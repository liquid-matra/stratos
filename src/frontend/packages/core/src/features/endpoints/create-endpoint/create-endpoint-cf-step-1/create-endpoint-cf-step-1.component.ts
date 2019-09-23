import { AfterContentInit, Component, Input, ViewChild } from '@angular/core';
import { NgForm, NgModel } from '@angular/forms';
import { ActivatedRoute } from '@angular/router';
import { Store } from '@ngrx/store';
import { denormalize } from 'normalizr';
import { Observable } from 'rxjs';
import { filter, map, pairwise, withLatestFrom } from 'rxjs/operators';

import { getIdFromRoute } from '../../../../../../cloud-foundry/src/features/cloud-foundry/cf.helpers';
import { GetAllEndpoints, RegisterEndpoint } from '../../../../../../store/src/actions/endpoint.actions';
import { ShowSnackBar } from '../../../../../../store/src/actions/snackBar.actions';
import { GeneralEntityAppState } from '../../../../../../store/src/app-state';
import { EndpointsEffect } from '../../../../../../store/src/effects/endpoint.effects';
import { endpointSchemaKey } from '../../../../../../store/src/helpers/entity-factory';
import { getAPIRequestDataState, selectUpdateInfo } from '../../../../../../store/src/selectors/api.selectors';
import { selectPaginationState } from '../../../../../../store/src/selectors/pagination.selectors';
import { endpointEntitySchema, STRATOS_ENDPOINT_TYPE } from '../../../../base-entity-schemas';
import { StratosCatalogueEndpointEntity } from '../../../../core/entity-catalogue/entity-catalogue-entity';
import { entityCatalogue } from '../../../../core/entity-catalogue/entity-catalogue.service';
import { IStepperStep, StepOnNextFunction } from '../../../../shared/components/stepper/step/step.component';
import { ConnectEndpointConfig } from '../../connect.service';
import { getFullEndpointApiUrl } from '../../endpoint-helpers';

/* tslint:disable:no-access-missing-member https://github.com/mgechev/codelyzer/issues/191*/
@Component({
  selector: 'app-create-endpoint-cf-step-1',
  templateUrl: './create-endpoint-cf-step-1.component.html',
  styleUrls: ['./create-endpoint-cf-step-1.component.scss']
})
export class CreateEndpointCfStep1Component implements IStepperStep, AfterContentInit {

  @Input() finalStep: boolean;

  existingEndpoints: Observable<{
    names: string[],
    urls: string[],
  }>;

  validate: Observable<boolean>;

  @ViewChild('form') form: NgForm;
  @ViewChild('nameField') nameField: NgModel;
  @ViewChild('urlField') urlField: NgModel;
  @ViewChild('skipSllField') skipSllField: NgModel;
  @ViewChild('ssoAllowedField') ssoAllowedField: NgModel;

  // Optional Client ID and Client Secret
  @ViewChild('clientIDField') clientIDField: NgModel;
  @ViewChild('clientSecretField') clientSecretField: NgModel;

  urlValidation: string;

  showAdvancedFields = false;
  clientRedirectURI: string;

  endpointTypeSupportsSSO = false;
  endpoint: StratosCatalogueEndpointEntity;

  private endpointEntityKey = entityCatalogue.getEntityKey(STRATOS_ENDPOINT_TYPE, endpointSchemaKey);

  constructor(private store: Store<GeneralEntityAppState>, activatedRoute: ActivatedRoute, ) {

    this.existingEndpoints = store.select(selectPaginationState(this.endpointEntityKey, GetAllEndpoints.storeKey))
      .pipe(
        withLatestFrom(store.select(getAPIRequestDataState)),
        map(([pagination, entities]) => {
          const pages = Object.values(pagination.ids);
          const page = [].concat.apply([], pages);
          const endpoints = page.length ? denormalize(page, [endpointEntitySchema], entities) : [];
          return {
            names: endpoints.map(ep => ep.name),
            urls: endpoints.map(ep => getFullEndpointApiUrl(ep)),
          };
        })
      );

    const epType = getIdFromRoute(activatedRoute, 'type');
    const epSubType = getIdFromRoute(activatedRoute, 'subtype');
    this.endpoint = entityCatalogue.getEndpoint(epType, epSubType);
    this.setUrlValidation(this.endpoint);

    // Client Redirect URI for SSO
    this.clientRedirectURI = window.location.protocol + '//' + window.location.hostname +
      (window.location.port ? ':' + window.location.port : '') + '/pp/v1/auth/sso_login_callback';
  }

  onNext: StepOnNextFunction = () => {
    const { subType, type } = this.endpoint.getTypeAndSubtype();
    const action = new RegisterEndpoint(
      type,
      subType,
      this.nameField.value,
      this.urlField.value,
      !!this.skipSllField.value,
      this.clientIDField ? this.clientIDField.value : '',
      this.clientSecretField ? this.clientSecretField.value : '',
      this.ssoAllowedField ? !!this.ssoAllowedField.value : false,
    );

    this.store.dispatch(action);

    const update$ = this.store.select(
      this.getUpdateSelector(action.guid())
    ).pipe(filter(update => !!update));

    return update$.pipe(pairwise(),
      filter(([oldVal, newVal]) => (oldVal.busy && !newVal.busy)),
      map(([oldVal, newVal]) => newVal),
      map(result => {
        const data: ConnectEndpointConfig = {
          guid: result.message,
          name: this.nameField.value,
          type,
          subType,
          ssoAllowed: this.ssoAllowedField ? !!this.ssoAllowedField.value : false
        };
        if (!result.error) {
          this.store.dispatch(new ShowSnackBar(`Successfully registered '${this.nameField.value}'`));
        }
        const success = !result.error;
        return {
          success,
          redirect: success && this.finalStep,
          message: success ? '' : result.message,
          data
        };
      })
    );
  }

  private getUpdateSelector(guid) {
    return selectUpdateInfo(
      this.endpointEntityKey,
      guid,
      EndpointsEffect.registeringKey,
    );
  }

  ngAfterContentInit() {
    this.validate = this.form.statusChanges.pipe(
      map(() => {
        return this.form.valid;
      }));
  }

  setUrlValidation(endpoint: StratosCatalogueEndpointEntity) {
    this.urlValidation = endpoint ? endpoint.definition.urlValidationRegexString : '';
    this.setAdvancedFields(endpoint);
  }

  // Only show the Client ID and Client Secret fields if the endpoint type is Cloud Foundry
  setAdvancedFields(endpoint: StratosCatalogueEndpointEntity) {
    this.showAdvancedFields = endpoint.definition.type === 'cf';

    // Only allow SSL if the endpoint type is Cloud Foundry
    this.endpointTypeSupportsSSO = endpoint.definition.type === 'cf';
  }
}