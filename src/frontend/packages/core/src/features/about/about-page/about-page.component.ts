import {
  Component,
  ComponentFactory,
  ComponentFactoryResolver,
  ComponentRef,
  OnDestroy,
  OnInit,
  ViewChild,
  ViewContainerRef,
} from '@angular/core';
import { Store } from '@ngrx/store';
import { Observable } from 'rxjs';
import { catchError, filter, map } from 'rxjs/operators';

import { gitEntityCatalog, GitSCMService } from '../../../../../git/src/public_api';
import { GeneralEntityAppState } from '../../../../../store/src/app-state';
import { AuthState } from '../../../../../store/src/reducers/auth.reducer';
import { SessionData } from '../../../../../store/src/types/auth.types';
import { CustomizationService, CustomizationsMetadata } from '../../../core/customizations.types';

@Component({
  selector: 'app-about-page',
  templateUrl: './about-page.component.html',
  styleUrls: ['./about-page.component.scss']
})
export class AboutPageComponent implements OnInit, OnDestroy {

  sessionData$: Observable<SessionData>;
  versionNumber$: Observable<string>;
  userIsAdmin$: Observable<boolean>;

  @ViewChild('aboutInfoContainer', { read: ViewContainerRef, static: true }) aboutInfoContainer;
  @ViewChild('supportInfoContainer', { read: ViewContainerRef, static: true }) supportInfoContainer;

  aboutInfoComponentRef: ComponentRef<any>;
  componentRef: ComponentRef<any>;

  customizations: CustomizationsMetadata;

  constructor(
    private store: Store<GeneralEntityAppState>,
    private resolver: ComponentFactoryResolver,
    cs: CustomizationService,
    private gitSCMService: GitSCMService,
  ) {
    this.customizations = cs.get();
  }

  ngOnInit() {
    this.sessionData$ = this.store.select(s => s.auth).pipe(
      filter(auth => !!(auth && auth.sessionData)),
      map((auth: AuthState) => auth.sessionData)
    );

    this.userIsAdmin$ = this.sessionData$.pipe(
      map(session => session.user && session.user.admin)
    );

    this.versionNumber$ = this.sessionData$.pipe(
      map((sessionData: SessionData) => {
        const versionNumber = sessionData.version.proxy_version;
        return versionNumber.split('-')[0];
      })
    );

    this.addAboutInfoComponent();
    this.addSupportInfo();


    gitEntityCatalog.branch.api.getMultiple(
      'Z6KJAVYeHPkiEPuZty0-kArqors',
      null,
      {
        projectName: 'richard-cox/my-private-repo',
        scm: this.gitSCMService.getSCM('github')
      }
    ).pipe(
      catchError(err => {
        console.error(err);
        return null;
      })
    ).subscribe(res => console.log(res));
  }

  ngOnDestroy() {
    if (this.aboutInfoComponentRef) {
      this.aboutInfoComponentRef.destroy();
    }
    if (this.componentRef) {
      this.componentRef.destroy();
    }
  }

  addAboutInfoComponent() {
    this.aboutInfoContainer.clear();
    if (this.customizations.aboutInfoComponent) {
      const factory: ComponentFactory<any> = this.resolver.resolveComponentFactory(this.customizations.aboutInfoComponent);
      this.aboutInfoComponentRef = this.aboutInfoContainer.createComponent(factory);
    }
  }

  addSupportInfo() {
    this.supportInfoContainer.clear();
    if (this.customizations.supportInfoComponent) {
      const factory: ComponentFactory<any> = this.resolver.resolveComponentFactory(this.customizations.supportInfoComponent);
      this.componentRef = this.supportInfoContainer.createComponent(factory);
    }
  }
}
